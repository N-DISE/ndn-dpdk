package ringbuffer_test

import (
	"testing"

	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ringbuffer"
)

func TestRing(t *testing.T) {
	assert, require := makeAR(t)

	r, e := ringbuffer.New(4, eal.NumaSocket{}, ringbuffer.ProducerMulti, ringbuffer.ConsumerMulti)
	require.NoError(e)
	defer r.Close()

	assert.NotEmpty(r.String())
	assert.Equal(0, r.CountInUse())
	assert.Equal(3, r.CountAvailable())
	assert.Equal(r.CountAvailable(), r.Capacity())

	output := make([]uintptr, 3)
	assert.Equal(0, r.Dequeue(output[:2]))

	input := []uintptr{9971, 3087}
	assert.Equal(2, r.Enqueue(input))
	assert.Equal(2, r.CountInUse())
	assert.Equal(1, r.CountAvailable())

	input = []uintptr{2776, 1876}
	assert.Equal(1, r.Enqueue(input))
	assert.Equal(3, r.CountInUse())
	assert.Equal(0, r.CountAvailable())

	assert.Equal(1, r.Dequeue(output[:1]))
	assert.Equal(uintptr(9971), output[0])
	assert.Equal(2, r.CountInUse())
	assert.Equal(1, r.CountAvailable())

	assert.Equal(2, r.Dequeue(output[:3]))
	assert.Equal(uintptr(3087), output[0])
	assert.Equal(uintptr(2776), output[1])
	assert.Equal(0, r.CountInUse())
	assert.Equal(3, r.CountAvailable())
}

func TestCapacity(t *testing.T) {
	assert, require := makeAR(t)

	r, e := ringbuffer.New(-1, eal.NumaSocket{}, ringbuffer.ProducerMulti, ringbuffer.ConsumerMulti)
	require.NoError(e)
	assert.Equal(63, r.Capacity())
	defer r.Close()

	r, e = ringbuffer.New(129, eal.NumaSocket{}, ringbuffer.ProducerMulti, ringbuffer.ConsumerMulti)
	require.NoError(e)
	assert.Equal(255, r.Capacity())
	defer r.Close()
}
